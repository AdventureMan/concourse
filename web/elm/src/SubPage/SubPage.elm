module SubPage.SubPage exposing
    ( Model(..)
    , handleCallback
    , handleDelivery
    , handleNotFound
    , init
    , subscriptions
    , update
    , urlUpdate
    , view
    )

import Build.Build as Build
import Build.Models
import Dashboard.Dashboard as Dashboard
import Dashboard.Models
import FlySuccess.FlySuccess as FlySuccess
import FlySuccess.Models
import Html exposing (Html)
import Job.Job as Job
import Message.Callback exposing (Callback)
import Message.Effects exposing (Effect)
import Message.Message exposing (Message(..))
import Message.Subscription exposing (Delivery(..), Interval(..), Subscription)
import NotFound.Model
import NotFound.NotFound as NotFound
import Pipeline.Pipeline as Pipeline
import Resource.Models
import Resource.Resource as Resource
import Routes
import UpdateMsg exposing (UpdateMsg)
import UserState exposing (UserState)


type Model
    = BuildModel Build.Models.Model
    | JobModel Job.Model
    | ResourceModel Resource.Models.Model
    | PipelineModel Pipeline.Model
    | NotFoundModel NotFound.Model.Model
    | DashboardModel Dashboard.Models.Model
    | FlySuccessModel FlySuccess.Models.Model


type alias Flags =
    { authToken : String
    , turbulencePath : String
    , pipelineRunningKeyframes : String
    }


init : Flags -> Routes.Route -> ( Model, List Effect )
init flags route =
    case route of
        Routes.Build { id, highlight } ->
            Build.init
                { highlight = highlight
                , pageType = Build.Models.JobBuildPage id
                }
                |> Tuple.mapFirst BuildModel

        Routes.OneOffBuild { id, highlight } ->
            Build.init
                { highlight = highlight
                , pageType = Build.Models.OneOffBuildPage id
                }
                |> Tuple.mapFirst BuildModel

        Routes.Resource { id, page } ->
            Resource.init
                { resourceId = id
                , paging = page
                }
                |> Tuple.mapFirst ResourceModel

        Routes.Job { id, page } ->
            Job.init
                { jobId = id
                , paging = page
                }
                |> Tuple.mapFirst JobModel

        Routes.Pipeline { id, groups } ->
            Pipeline.init
                { pipelineLocator = id
                , turbulenceImgSrc = flags.turbulencePath
                , selectedGroups = groups
                }
                |> Tuple.mapFirst PipelineModel

        Routes.Dashboard searchType ->
            Dashboard.init
                { turbulencePath = flags.turbulencePath
                , searchType = searchType
                , pipelineRunningKeyframes = flags.pipelineRunningKeyframes
                }
                |> Tuple.mapFirst DashboardModel

        Routes.FlySuccess { flyPort } ->
            FlySuccess.init
                { authToken = flags.authToken
                , flyPort = flyPort
                }
                |> Tuple.mapFirst FlySuccessModel


handleNotFound : String -> Routes.Route -> ( Model, List Effect ) -> ( Model, List Effect )
handleNotFound notFound route ( model, effects ) =
    case getUpdateMessage model of
        UpdateMsg.NotFound ->
            let
                ( newModel, newEffects ) =
                    NotFound.init { notFoundImgSrc = notFound, route = route }
            in
            ( NotFoundModel newModel, effects ++ newEffects )

        UpdateMsg.AOK ->
            ( model, effects )


getUpdateMessage : Model -> UpdateMsg
getUpdateMessage model =
    case model of
        BuildModel mdl ->
            Build.getUpdateMessage mdl

        JobModel mdl ->
            Job.getUpdateMessage mdl

        ResourceModel mdl ->
            Resource.getUpdateMessage mdl

        PipelineModel mdl ->
            Pipeline.getUpdateMessage mdl

        _ ->
            UpdateMsg.AOK


genericUpdate :
    (( Build.Models.Model, List Effect ) -> ( Build.Models.Model, List Effect ))
    -> (( Job.Model, List Effect ) -> ( Job.Model, List Effect ))
    -> (( Resource.Models.Model, List Effect ) -> ( Resource.Models.Model, List Effect ))
    -> (( Pipeline.Model, List Effect ) -> ( Pipeline.Model, List Effect ))
    -> (( Dashboard.Models.Model, List Effect ) -> ( Dashboard.Models.Model, List Effect ))
    -> (( NotFound.Model.Model, List Effect ) -> ( NotFound.Model.Model, List Effect ))
    -> (( FlySuccess.Models.Model, List Effect ) -> ( FlySuccess.Models.Model, List Effect ))
    -> ( Model, List Effect )
    -> ( Model, List Effect )
genericUpdate fBuild fJob fRes fPipe fDash fNF fFS ( model, effects ) =
    case model of
        BuildModel model ->
            fBuild ( model, effects )
                |> Tuple.mapFirst BuildModel

        JobModel model ->
            fJob ( model, effects )
                |> Tuple.mapFirst JobModel

        PipelineModel model ->
            fPipe ( model, effects )
                |> Tuple.mapFirst PipelineModel

        ResourceModel model ->
            fRes ( model, effects )
                |> Tuple.mapFirst ResourceModel

        DashboardModel model ->
            fDash ( model, effects )
                |> Tuple.mapFirst DashboardModel

        FlySuccessModel model ->
            fFS ( model, effects )
                |> Tuple.mapFirst FlySuccessModel

        NotFoundModel model ->
            fNF ( model, effects )
                |> Tuple.mapFirst NotFoundModel


handleCallback : Callback -> ( Model, List Effect ) -> ( Model, List Effect )
handleCallback callback =
    genericUpdate
        (Build.handleCallback callback)
        (Job.handleCallback callback)
        (Resource.handleCallback callback)
        (Pipeline.handleCallback callback)
        (Dashboard.handleCallback callback)
        (NotFound.handleCallback callback)
        (FlySuccess.handleCallback callback)


handleDelivery : Delivery -> ( Model, List Effect ) -> ( Model, List Effect )
handleDelivery delivery =
    genericUpdate
        (Build.handleDelivery delivery)
        (Job.handleDelivery delivery)
        (Resource.handleDelivery delivery)
        (Pipeline.handleDelivery delivery)
        (Dashboard.handleDelivery delivery)
        identity
        identity


update : Message -> ( Model, List Effect ) -> ( Model, List Effect )
update msg =
    genericUpdate
        (Build.update msg)
        (Job.update msg)
        (Resource.update msg)
        (Pipeline.update msg)
        (Dashboard.update msg)
        (NotFound.update msg)
        (FlySuccess.update msg)


urlUpdate : Routes.Route -> ( Model, List Effect ) -> ( Model, List Effect )
urlUpdate route =
    genericUpdate
        (case route of
            Routes.Build { id, highlight } ->
                Build.changeToBuild
                    { pageType = Build.Models.JobBuildPage id
                    , highlight = highlight
                    }

            _ ->
                identity
        )
        (case route of
            Routes.Job { id, page } ->
                Job.changeToJob { jobId = id, paging = page }

            _ ->
                identity
        )
        (case route of
            Routes.Resource { id, page } ->
                Resource.changeToResource { resourceId = id, paging = page }

            _ ->
                identity
        )
        (case route of
            Routes.Pipeline { id, groups } ->
                Pipeline.changeToPipelineAndGroups
                    { pipelineLocator = id
                    , selectedGroups = groups
                    }

            _ ->
                identity
        )
        identity
        identity
        identity


view : UserState -> Model -> Html Message
view userState mdl =
    case mdl of
        BuildModel model ->
            Build.view userState model

        JobModel model ->
            Job.view userState model

        PipelineModel model ->
            Pipeline.view userState model

        ResourceModel model ->
            Resource.view userState model

        DashboardModel model ->
            Dashboard.view userState model

        NotFoundModel model ->
            NotFound.view userState model

        FlySuccessModel model ->
            FlySuccess.view userState model


subscriptions : Model -> List Subscription
subscriptions mdl =
    case mdl of
        BuildModel model ->
            Build.subscriptions model

        JobModel model ->
            Job.subscriptions model

        PipelineModel model ->
            Pipeline.subscriptions model

        ResourceModel model ->
            Resource.subscriptions model

        DashboardModel model ->
            Dashboard.subscriptions model

        NotFoundModel _ ->
            []

        FlySuccessModel _ ->
            []
